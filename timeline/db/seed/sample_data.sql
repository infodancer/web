-- Sample timeline: World War I key events

INSERT INTO timelines (slug, name, description, start_date, end_date, is_public)
VALUES ('wwi', 'World War I', 'Key events of the First World War, 1914-1918', '1914-06-28', '1918-11-11', true);

INSERT INTO categories (timeline_id, slug, name, color, sort_order) VALUES
    (1, 'political', 'Political', '#ef4444', 1),
    (1, 'military', 'Military', '#3b82f6', 2),
    (1, 'diplomatic', 'Diplomatic', '#8b5cf6', 3),
    (1, 'naval', 'Naval', '#06b6d4', 4),
    (1, 'technology', 'Technology', '#f59e0b', 5);

INSERT INTO tags (slug, name) VALUES
    ('western-front', 'Western Front'),
    ('eastern-front', 'Eastern Front'),
    ('home-front', 'Home Front'),
    ('casualties', 'Casualties'),
    ('turning-point', 'Turning Point');

INSERT INTO events (timeline_id, slug, title, summary, article, event_date, date_precision, sort_order, category_id) VALUES
    (1, 'assassination-archduke', 'Assassination of Archduke Franz Ferdinand',
     'Archduke Franz Ferdinand of Austria and his wife Sophie are assassinated in Sarajevo by Gavrilo Princip.',
     'On June 28, 1914, Archduke Franz Ferdinand of Austria, heir presumptive to the Austro-Hungarian throne, and his wife Sophie, Duchess of Hohenberg, were assassinated by Gavrilo Princip, a Bosnian Serb nationalist. The assassination set off a chain of international events that led to the outbreak of World War I.',
     '1914-06-28', 'day', 0, 1),

    (1, 'austria-ultimatum', 'Austria-Hungary Issues Ultimatum to Serbia',
     'Austria-Hungary delivers a harsh ultimatum to Serbia with deliberately unacceptable terms.',
     'The July Ultimatum was a list of ten demands sent from Austria-Hungary to Serbia on July 23, 1914. Serbia accepted all but one of the demands, but this partial rejection was used as justification for Austria-Hungary to declare war.',
     '1914-07-23', 'day', 0, 3),

    (1, 'austria-declares-war', 'Austria-Hungary Declares War on Serbia',
     'Austria-Hungary formally declares war on Serbia, beginning the chain of alliance activations.',
     '',
     '1914-07-28', 'day', 0, 2),

    (1, 'germany-declares-war-russia', 'Germany Declares War on Russia',
     'Germany declares war on Russia after Russian mobilization.',
     '',
     '1914-08-01', 'day', 0, 2),

    (1, 'germany-declares-war-france', 'Germany Declares War on France',
     'Germany declares war on France and invades Belgium.',
     '',
     '1914-08-03', 'day', 0, 2),

    (1, 'britain-declares-war', 'Britain Declares War on Germany',
     'Britain enters the war after Germany violates Belgian neutrality.',
     'Following Germany''s invasion of Belgium, Britain issued an ultimatum demanding withdrawal. When the ultimatum expired at midnight, Britain declared war on Germany, bringing the British Empire into the conflict.',
     '1914-08-04', 'day', 0, 3),

    (1, 'battle-of-marne', 'First Battle of the Marne',
     'Allied forces halt the German advance on Paris, ending the Schlieffen Plan.',
     'The First Battle of the Marne was fought from September 5 to 12, 1914. It resulted in an Allied victory against the German armies in the west. The battle effectively ended the German advance into France and the Schlieffen Plan, leading to the development of trench warfare on the Western Front.',
     '1914-09-05', 'day', 0, 2),

    (1, 'christmas-truce', 'Christmas Truce',
     'Unofficial ceasefires occur along the Western Front as soldiers from both sides celebrate Christmas together.',
     '',
     '1914-12-25', 'day', 0, 2),

    (1, 'gallipoli-campaign', 'Gallipoli Campaign Begins',
     'Allied forces launch an amphibious assault on the Gallipoli peninsula.',
     'The Gallipoli campaign was a military campaign in which Allied forces attempted to control the sea route from Europe to Russia via the Dardanelles strait. The campaign began with a naval attack followed by an amphibious landing on the Gallipoli peninsula in April 1915.',
     '1915-04-25', 'day', 0, 2),

    (1, 'lusitania-sinking', 'Sinking of the Lusitania',
     'German submarine U-20 torpedoes the British ocean liner RMS Lusitania, killing 1,198 passengers.',
     'The sinking of the Lusitania on May 7, 1915, was a pivotal moment in World War I. The loss of 128 American lives contributed to turning public opinion in the United States against Germany and was a factor in the eventual American entry into the war.',
     '1915-05-07', 'day', 0, 4),

    (1, 'battle-of-verdun', 'Battle of Verdun Begins',
     'Germany launches a massive offensive against French positions at Verdun.',
     'The Battle of Verdun was one of the longest and most devastating battles of World War I. It lasted from February 21 to December 18, 1916, and resulted in approximately 700,000 combined casualties.',
     '1916-02-21', 'day', 0, 2),

    (1, 'battle-of-jutland', 'Battle of Jutland',
     'The largest naval battle of World War I takes place in the North Sea.',
     'The Battle of Jutland was fought between the British Grand Fleet and the German High Seas Fleet on May 31 to June 1, 1916. Though tactically indecisive, it confirmed British naval dominance for the remainder of the war.',
     '1916-05-31', 'day', 0, 4),

    (1, 'battle-of-somme', 'Battle of the Somme Begins',
     'British and French forces launch a massive offensive along the Somme river. The first day sees 57,470 British casualties.',
     'The Battle of the Somme was one of the bloodiest battles in human history. The first day, July 1, 1916, saw the British Army suffer its worst single-day losses. The battle continued until November 18, 1916, with over one million total casualties on all sides.',
     '1916-07-01', 'day', 0, 2),

    (1, 'tanks-first-used', 'Tanks First Used in Battle',
     'British forces deploy tanks for the first time during the Battle of the Somme.',
     'On September 15, 1916, British Mark I tanks were used for the first time in battle at Flers-Courcelette during the Battle of the Somme. Though mechanically unreliable, they demonstrated the potential for armored warfare.',
     '1916-09-15', 'day', 0, 5),

    (1, 'zimmermann-telegram', 'Zimmermann Telegram Intercepted',
     'British intelligence intercepts a German diplomatic communication proposing a military alliance with Mexico against the United States.',
     'The Zimmermann Telegram was a secret diplomatic communication issued from the German Foreign Office in January 1917 that proposed a military alliance between Germany and Mexico. Its interception and publication helped draw the United States into the war.',
     '1917-01-16', 'day', 0, 3),

    (1, 'us-enters-war', 'United States Enters the War',
     'The US Congress declares war on Germany following unrestricted submarine warfare and the Zimmermann Telegram.',
     'On April 6, 1917, the United States formally declared war on Germany. President Woodrow Wilson had campaigned for reelection on a platform of neutrality, but unrestricted submarine warfare and the Zimmermann Telegram shifted public opinion decisively.',
     '1917-04-06', 'day', 0, 1),

    (1, 'passchendaele', 'Battle of Passchendaele Begins',
     'British forces launch the Third Battle of Ypres in Flanders.',
     'The Battle of Passchendaele, also known as the Third Battle of Ypres, was fought from July 31 to November 10, 1917. It became infamous for the appalling conditions of mud and the enormous casualties suffered for minimal territorial gains.',
     '1917-07-31', 'day', 0, 2),

    (1, 'russian-revolution', 'Russian Revolution',
     'The Bolshevik Revolution overthrows the Russian Provisional Government, leading to Russia''s withdrawal from the war.',
     '',
     '1917-11-07', 'day', 0, 1),

    (1, 'fourteen-points', 'Wilson''s Fourteen Points',
     'President Wilson outlines his vision for post-war peace in a speech to Congress.',
     'On January 8, 1918, President Woodrow Wilson addressed Congress with his Fourteen Points, a statement of principles for peace that was to be used for negotiations to end World War I. The points included open diplomacy, freedom of navigation, and the establishment of a League of Nations.',
     '1918-01-08', 'day', 0, 3),

    (1, 'spring-offensive', 'German Spring Offensive',
     'Germany launches a series of massive attacks on the Western Front in a final bid for victory.',
     'The German Spring Offensive of 1918, also known as the Kaiserschlacht, was a series of German attacks along the Western Front beginning on March 21, 1918. Though initially successful, the offensive ultimately failed to achieve a decisive breakthrough.',
     '1918-03-21', 'day', 0, 2),

    (1, 'hundred-days-offensive', 'Hundred Days Offensive Begins',
     'Allied forces launch a series of attacks that would end the war.',
     'The Hundred Days Offensive was a series of massive Allied offensives that began on August 8, 1918. This date was described by Erich Ludendorff as "the black day of the German Army." The offensive continued until the Armistice on November 11.',
     '1918-08-08', 'day', 0, 2),

    (1, 'armistice', 'Armistice Day',
     'The Armistice is signed at 5:45 AM and takes effect at 11:00 AM, ending the fighting on the Western Front.',
     'The Armistice of November 11, 1918, was the agreement that ended fighting on the Western Front. It was signed at 5:45 AM in Marshal Foch''s railway carriage in the Forest of Compi&egrave;gne and took effect at "the eleventh hour of the eleventh day of the eleventh month." The war had claimed approximately 20 million lives.',
     '1918-11-11', 'day', 0, 3);

-- Tag associations
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'battle-of-marne' AND t.slug = 'western-front';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'battle-of-marne' AND t.slug = 'turning-point';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'battle-of-verdun' AND t.slug = 'western-front';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'battle-of-verdun' AND t.slug = 'casualties';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'battle-of-somme' AND t.slug = 'western-front';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'battle-of-somme' AND t.slug = 'casualties';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'us-enters-war' AND t.slug = 'turning-point';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'hundred-days-offensive' AND t.slug = 'western-front';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'hundred-days-offensive' AND t.slug = 'turning-point';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'russian-revolution' AND t.slug = 'eastern-front';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'russian-revolution' AND t.slug = 'turning-point';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'lusitania-sinking' AND t.slug = 'home-front';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'passchendaele' AND t.slug = 'western-front';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'passchendaele' AND t.slug = 'casualties';
INSERT INTO event_tags (event_id, tag_id)
SELECT e.id, t.id FROM events e, tags t WHERE e.slug = 'christmas-truce' AND t.slug = 'western-front';

-- People
INSERT INTO people (timeline_id, slug, name, sort_name, bio, born_date, died_date) VALUES
    (1, 'gavrilo-princip', 'Gavrilo Princip', 'Princip, Gavrilo',
     'Bosnian Serb nationalist who assassinated Archduke Franz Ferdinand and his wife Sophie, triggering the chain of events that led to World War I. A member of the Young Bosnia movement, Princip was 19 at the time of the assassination. He was sentenced to 20 years in prison, as he was too young for the death penalty under Austro-Hungarian law, and died of tuberculosis in prison in 1918.',
     '1894-07-25', '1918-04-28'),

    (1, 'franz-ferdinand', 'Archduke Franz Ferdinand', 'Franz Ferdinand',
     'Heir presumptive to the Austro-Hungarian throne. His assassination in Sarajevo on June 28, 1914, was the immediate catalyst for World War I. He had advocated for greater autonomy for the Slavic peoples of the empire, which ironically made him a target for Serbian nationalists who feared such reforms would reduce the appeal of a Greater Serbia.',
     '1863-12-18', '1914-06-28'),

    (1, 'woodrow-wilson', 'Woodrow Wilson', 'Wilson, Woodrow',
     'The 28th President of the United States. Initially committed to neutrality, Wilson led America into the war in April 1917 and later crafted the Fourteen Points as a framework for peace. He championed the League of Nations, though the US Senate ultimately rejected American membership.',
     '1856-12-28', '1924-02-03'),

    (1, 'ferdinand-foch', 'Ferdinand Foch', 'Foch, Ferdinand',
     'French general who served as Supreme Commander of the Allied Armies from March 1918. He coordinated the final Allied offensives that led to the Armistice. The Armistice was signed in his railway carriage in the Forest of Compiegne.',
     '1851-10-02', '1929-03-20'),

    (1, 'erich-ludendorff', 'Erich Ludendorff', 'Ludendorff, Erich',
     'German general who, alongside Hindenburg, directed Germany''s war effort from 1916. He planned the Spring Offensive of 1918 and later described August 8, 1918 as "the black day of the German Army" when the Hundred Days Offensive began.',
     '1865-04-09', '1937-12-20'),

    (1, 'douglas-haig', 'Douglas Haig', 'Haig, Douglas',
     'Commander of the British Expeditionary Force on the Western Front from December 1915. He led British forces through the Somme, Passchendaele, and the Hundred Days Offensive. Remains one of the most controversial figures of the war, criticized for the enormous casualties under his command.',
     '1861-06-19', '1928-01-29'),

    (1, 'arthur-zimmermann', 'Arthur Zimmermann', 'Zimmermann, Arthur',
     'German Foreign Secretary who sent the infamous Zimmermann Telegram in January 1917, proposing a military alliance between Germany and Mexico against the United States. When the telegram was intercepted and published, it helped turn American public opinion toward entering the war.',
     '1864-10-05', '1940-06-06'),

    (1, 'vladimir-lenin', 'Vladimir Lenin', 'Lenin, Vladimir',
     'Leader of the Bolshevik Revolution in November 1917, which overthrew the Russian Provisional Government. He negotiated the Treaty of Brest-Litovsk in March 1918, taking Russia out of the war at enormous territorial cost.',
     '1870-04-22', '1924-01-21');

-- Event-People links
-- Assassination
INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'Assassin', 'Carried out the assassination that triggered the war', 1
FROM events e, people p WHERE e.slug = 'assassination-archduke' AND p.slug = 'gavrilo-princip';

INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'Victim', 'His assassination was the immediate trigger for the war', 2
FROM events e, people p WHERE e.slug = 'assassination-archduke' AND p.slug = 'franz-ferdinand';

-- US enters war
INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'President', 'Asked Congress to declare war on Germany', 1
FROM events e, people p WHERE e.slug = 'us-enters-war' AND p.slug = 'woodrow-wilson';

-- Fourteen Points
INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'Author', 'Drafted and delivered the Fourteen Points speech to Congress', 1
FROM events e, people p WHERE e.slug = 'fourteen-points' AND p.slug = 'woodrow-wilson';

-- Zimmermann Telegram
INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'Author', 'Sent the telegram proposing a German-Mexican alliance against the US', 1
FROM events e, people p WHERE e.slug = 'zimmermann-telegram' AND p.slug = 'arthur-zimmermann';

-- Battle of the Somme
INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'British Commander', 'Commanded British forces; oversaw the bloodiest day in British military history', 1
FROM events e, people p WHERE e.slug = 'battle-of-somme' AND p.slug = 'douglas-haig';

-- Passchendaele
INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'British Commander', 'Planned and directed the offensive despite deteriorating conditions', 1
FROM events e, people p WHERE e.slug = 'passchendaele' AND p.slug = 'douglas-haig';

-- Spring Offensive
INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'Planner', 'Planned the offensive as Germany''s last bid for victory', 1
FROM events e, people p WHERE e.slug = 'spring-offensive' AND p.slug = 'erich-ludendorff';

INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'Allied Supreme Commander', 'Coordinated the Allied defense that ultimately stopped the offensive', 2
FROM events e, people p WHERE e.slug = 'spring-offensive' AND p.slug = 'ferdinand-foch';

-- Hundred Days Offensive
INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'Allied Supreme Commander', 'Directed the series of Allied offensives that ended the war', 1
FROM events e, people p WHERE e.slug = 'hundred-days-offensive' AND p.slug = 'ferdinand-foch';

INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'British Commander', 'Led British forces in the decisive final offensives', 2
FROM events e, people p WHERE e.slug = 'hundred-days-offensive' AND p.slug = 'douglas-haig';

INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'German Commander', 'Called August 8 "the black day of the German Army"', 3
FROM events e, people p WHERE e.slug = 'hundred-days-offensive' AND p.slug = 'erich-ludendorff';

-- Russian Revolution
INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'Revolutionary Leader', 'Led the Bolshevik seizure of power and later took Russia out of the war', 1
FROM events e, people p WHERE e.slug = 'russian-revolution' AND p.slug = 'vladimir-lenin';

-- Armistice
INSERT INTO event_people (event_id, person_id, role, explainer, sort_order)
SELECT e.id, p.id, 'Allied Supreme Commander', 'The Armistice was signed in his railway carriage', 1
FROM events e, people p WHERE e.slug = 'armistice' AND p.slug = 'ferdinand-foch';

-- Event links (event_a_id < event_b_id enforced by CHECK constraint)
-- Assassination -> Austria ultimatum -> Austria declares war (causal chain)
INSERT INTO event_links (event_a_id, event_b_id, relationship, description)
SELECT a.id, b.id, 'caused', 'The assassination prompted Austria-Hungary''s ultimatum to Serbia'
FROM events a, events b WHERE a.slug = 'assassination-archduke' AND b.slug = 'austria-ultimatum';

INSERT INTO event_links (event_a_id, event_b_id, relationship, description)
SELECT a.id, b.id, 'caused', 'Serbia''s partial rejection of the ultimatum was used to justify war'
FROM events a, events b WHERE a.slug = 'austria-ultimatum' AND b.slug = 'austria-declares-war';

-- Lusitania + Zimmermann -> US enters war
INSERT INTO event_links (event_a_id, event_b_id, relationship, description)
SELECT a.id, b.id, 'contributed to', 'The loss of 128 American lives shifted US public opinion against Germany'
FROM events a, events b WHERE a.slug = 'lusitania-sinking' AND b.slug = 'us-enters-war';

INSERT INTO event_links (event_a_id, event_b_id, relationship, description)
SELECT a.id, b.id, 'caused', 'The telegram''s publication was a decisive factor in the US declaration of war'
FROM events a, events b WHERE a.slug = 'zimmermann-telegram' AND b.slug = 'us-enters-war';

-- US enters -> Fourteen Points
INSERT INTO event_links (event_a_id, event_b_id, relationship, description)
SELECT a.id, b.id, 'preceded', 'Wilson outlined his peace vision nine months after entering the war'
FROM events a, events b WHERE a.slug = 'us-enters-war' AND b.slug = 'fourteen-points';

-- Spring Offensive -> Hundred Days -> Armistice
INSERT INTO event_links (event_a_id, event_b_id, relationship, description)
SELECT a.id, b.id, 'caused', 'The failure of the Spring Offensive left Germany vulnerable to the Allied counterattack'
FROM events a, events b WHERE a.slug = 'spring-offensive' AND b.slug = 'hundred-days-offensive';

INSERT INTO event_links (event_a_id, event_b_id, relationship, description)
SELECT a.id, b.id, 'caused', 'The Hundred Days Offensive forced Germany to seek an armistice'
FROM events a, events b WHERE a.slug = 'hundred-days-offensive' AND b.slug = 'armistice';

-- Marne -> trench warfare context
INSERT INTO event_links (event_a_id, event_b_id, relationship, description)
SELECT a.id, b.id, 'related', 'Both major Western Front battles defined by the trench warfare the Marne initiated'
FROM events a, events b WHERE a.slug = 'battle-of-marne' AND b.slug = 'battle-of-verdun';

-- Somme -> tanks first used
INSERT INTO event_links (event_a_id, event_b_id, relationship, description)
SELECT a.id, b.id, 'related', 'Tanks were first deployed during the later stages of the Somme campaign'
FROM events a, events b WHERE a.slug = 'battle-of-somme' AND b.slug = 'tanks-first-used';

-- Sources
INSERT INTO event_sources (event_id, url, title, author, citation, sort_order)
SELECT e.id, 'https://en.wikipedia.org/wiki/Assassination_of_Archduke_Franz_Ferdinand',
    'Assassination of Archduke Franz Ferdinand', '', 'Wikipedia', 1
FROM events e WHERE e.slug = 'assassination-archduke';

INSERT INTO event_sources (event_id, url, title, author, citation, sort_order)
SELECT e.id, 'https://www.britannica.com/event/Battle-of-the-Somme',
    'Battle of the Somme', '', 'Encyclopaedia Britannica', 1
FROM events e WHERE e.slug = 'battle-of-somme';

INSERT INTO event_sources (event_id, url, title, author, citation, sort_order)
SELECT e.id, 'https://www.archives.gov/historical-docs/todays-doc/?dod-date=108',
    'Zimmermann Telegram', '', 'US National Archives', 1
FROM events e WHERE e.slug = 'zimmermann-telegram';

INSERT INTO event_sources (event_id, url, title, author, citation, sort_order)
SELECT e.id, 'https://avalon.law.yale.edu/20th_century/wilson14.asp',
    'President Woodrow Wilson''s Fourteen Points', '', 'The Avalon Project, Yale Law School', 1
FROM events e WHERE e.slug = 'fourteen-points';

INSERT INTO event_sources (event_id, url, title, author, citation, sort_order)
SELECT e.id, 'https://www.iwm.org.uk/history/the-christmas-truce',
    'The Christmas Truce', '', 'Imperial War Museums', 1
FROM events e WHERE e.slug = 'christmas-truce';

INSERT INTO event_sources (event_id, url, title, author, citation, sort_order)
SELECT e.id, 'https://www.britannica.com/event/Battle-of-Verdun',
    'Battle of Verdun', '', 'Encyclopaedia Britannica', 1
FROM events e WHERE e.slug = 'battle-of-verdun';
